import {
  Table,
  Button,
  Modal,
  TextInput,
  Label,
  Spinner,
} from "flowbite-react";
import { useForm, useFieldArray, SubmitHandler } from "react-hook-form";
import { joiResolver } from "@hookform/resolvers/joi";
import Joi from "joi";
import { HiAdjustments, HiTrash, HiDocumentAdd } from "react-icons/hi";
import React from "react";
import http, { RequestResponse } from "../utils/axios";

type Domain = {
  fqdn: string;
};

type UpstreamForm = {
  backend: string;
};

type FormValues = {
  domains: Domain[];
  upstreams: UpstreamForm[];
  matcher: string | undefined;
};

interface Upstream {
  ID: number;
  CreatedAt: string;
  UpdatedAt: string;
  DeletedAt?: any;
  hostId: number;
  backend: string;
}

interface Hosts {
  ID: number;
  CreatedAt: string;
  UpdatedAt: string;
  DeletedAt?: any;
  domains: string;
  matcher: string;
  Upstreams: Upstream[];
}

interface RootObject {
  result: Hosts[];
}

function HostsPage() {
  const [modal, setModal] = React.useState(false);
  const [hostData, setHostData] = React.useState<RootObject>();
  const [loading, setLoading] = React.useState(false);

  const schema = Joi.object<FormValues>({
    matcher: Joi.any().optional(),
    upstreams: Joi.array().items(
      Joi.object().keys({
        backend: Joi.string().required(),
      })
    ),
    domains: Joi.array().items(
      Joi.object().keys({
        fqdn: Joi.string().required(),
      })
    ),
  });

  const {
    register,
    handleSubmit,
    control,
    formState: { errors },
  } = useForm({
    resolver: joiResolver(schema),
    defaultValues: {
      matcher: "",
      domains: [{ fqdn: "" }],
      upstreams: [{ backend: "" }],
    },
  });
  const {
    fields: upstreamFields,
    append: upstreamAppend,
    remove: upstreamRemove,
  } = useFieldArray({
    control,
    name: "upstreams",
  });

  const {
    fields: domainFields,
    append: domainAppend,
    remove: domainRemove,
  } = useFieldArray({
    control,
    name: "domains",
  });

  React.useEffect(() => {
    getHosts();
  }, []);

  const getHosts = async () => {
    return await http.get<RequestResponse>(`/hosts`)
      .then((res) => {
        setHostData(res.data);
      });
  };

  const createHost: SubmitHandler<FormValues> = async (data) => {
    const jsonBody = {
      matcher: data.matcher,
      domains: data.domains
        .map((e) => {
          return e.fqdn;
        })
        .join(","),
      upstreams: data.upstreams,
    };
    setLoading(true);
    await http.post(`hosts`, jsonBody);
    setLoading(false);
    setModal(false);
    getHosts();
  };

  const deleteHost = async (hostID: number) => {
    await http.delete(`/hosts/${hostID}`);
    getHosts();
  };

  return (
    <>
      <div className="pb-4 bg-white dark:bg-gray-900">
        <Button size="xs" color="gray" onClick={() => setModal(true)}>
          <HiDocumentAdd className="mr-3 h-4 w-4" /> Add Host
        </Button>
      </div>
      <Table>
        <Table.Head>
          <Table.HeadCell>ID</Table.HeadCell>
          <Table.HeadCell>Created</Table.HeadCell>
          <Table.HeadCell>Updated</Table.HeadCell>
          <Table.HeadCell>Domains</Table.HeadCell>
          <Table.HeadCell>Matcher</Table.HeadCell>
          <Table.HeadCell>Upstreams</Table.HeadCell>
          <Table.HeadCell>
            <span className="sr-only">Edit</span>
          </Table.HeadCell>
        </Table.Head>
        <Table.Body className="divide-y">
          {hostData?.result.map((entry, i) => {
            return (
              <Table.Row
                key={entry.ID}
                className="bg-white dark:border-gray-700 dark:bg-gray-800"
              >
                <Table.Cell>{entry.ID}</Table.Cell>
                <Table.Cell>{entry.CreatedAt}</Table.Cell>
                <Table.Cell>{entry.UpdatedAt}</Table.Cell>
                <Table.Cell>{entry.domains}</Table.Cell>
                <Table.Cell>{entry.matcher}</Table.Cell>
                <Table.Cell>{entry.Upstreams[0].backend}</Table.Cell>
                <Table.Cell>
                  <Button.Group>
                    <Button size="xs" color="gray">
                      <HiAdjustments className="mr-3 h-4 w-4" /> Edit
                    </Button>
                    <Button
                      size="xs"
                      color="gray"
                      onClick={() => deleteHost(entry.ID)}
                    >
                      <HiTrash className="mr-3 h-4 w-4" /> Delete
                    </Button>
                  </Button.Group>
                </Table.Cell>
              </Table.Row>
            );
          })}
        </Table.Body>
      </Table>

      <Modal show={modal} size="xl" onClose={() => setModal(false)}>
        <Modal.Header>Add a new Host</Modal.Header>
        <Modal.Body>
          <form
            className="flex flex-col gap-4"
            onSubmit={handleSubmit(createHost)}
          >
            <div className="space-y-2">
              <div className="mb-2 block">
                <Label htmlFor="domain" value="Domain" />
              </div>
              <ul className="space-y-2">
                {domainFields.map((field, index) => {
                  return (
                    <li key={field.id}>
                      <TextInput
                        type="text"
                        placeholder="example.com"
                        id={`domains.${index}.fqdn`}
                        key={field.id}
                        {...register(`domains.${index}.fqdn` as const)}
                        addon={
                          index > 0 && (
                            <Button size="xs" color="gray" onClick={() => domainRemove(index)}>
                              Delete
                            </Button>
                          )
                        }
                        helperText={
                          errors.domains?.[index] && (
                            <span>Please enter a valid FQDN</span>
                          )
                        }
                      />
                    </li>
                  );
                })}
              </ul>
              <div className="mb-2 block">
                <Label value="Matcher" />
              </div>
              <TextInput
                type="text"
                id="matcher"
                placeholder="/api/*"
                {...register("matcher", { required: false })}
              />
              <div className="mb-2 block">
                <Label value="Upstreams" />
              </div>
              <ul className="space-y-2">
                {upstreamFields.map((field, index) => {
                  return (
                    <li key={field.id}>
                      <TextInput
                        type="text"
                        placeholder="127.0.0.1:8080"
                        id={`upstreams.${index}.backend`}
                        key={field.id}
                        {...register(`upstreams.${index}.backend` as const)}
                        addon={
                          index > 0 && (
                            <Button size="xs" color="gray" onClick={() => upstreamRemove(index)}>
                              Delete
                            </Button>
                          )
                        }
                        helperText={
                          errors.upstreams?.[index] && (
                            <span>Please enter a valid IP Address</span>
                          )
                        }
                      />
                    </li>
                  );
                })}
              </ul>
            </div>
            <Button.Group>
              {loading ? (
                <Button disabled={true}>
                  <div className="mr-3">
                    <Spinner size="sm" light={true} />
                  </div>
                  Loading ...
                </Button>
              ) : (
                <Button type="submit">Save</Button>
              )}

              <Button
                disabled={loading}
                type="button"
                onClick={() => domainAppend({ fqdn: "" })}
              >
                Add Domain
              </Button>
              <Button
                disabled={loading}
                type="button"
                onClick={() => upstreamAppend({ backend: "" })}
              >
                Add Upstream
              </Button>
            </Button.Group>
          </form>
        </Modal.Body>
      </Modal>
    </>
  );
}

export default HostsPage;
